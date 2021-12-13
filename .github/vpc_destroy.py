#!/usr/bin/env python
# description : Delete a specific AWS VPC with boto3
# author      : Jeff Loughridge
# e-mail      : jeffl@konekti.us
# create date : Mar 1, 2019
# modify date : Nov 02, 2021
# credits to https://github.com/jeffbrl/aws-vpc-destroy

import argparse
import logging
import os
import time
import sys

import boto3
from botocore.exceptions import ClientError

logger = logging.getLogger("root")
FORMAT = "[%(filename)s:%(lineno)s - %(funcName)20s() ] %(message)s"
logging.basicConfig(format=FORMAT, level=logging.INFO)


def destroy_ec2(vpc_id, aws_region):
    logger.debug(f"{vpc_id}")
    ec2 = boto3.resource("ec2", region_name=aws_region)
    ec2client = ec2.meta.client
    # test for valid credentials
    try:
        ec2client.describe_vpcs()
    except ClientError as e:
        logging.info(e)
        print(
            "Either your credentials are invalid or your IAM user doesn't have permissions to list VPCs"
        )
        sys.exit(1)

    if not vpc_exists(ec2client, vpc_id):
        print(f"VPC {vpc_id} does not exist in {aws_region}")
        return

    vpc = ec2.Vpc(vpc_id)

    # disassociate EIPs and release EIPs from EC2 instances
    for subnet in vpc.subnets.all():
        for instance in subnet.instances.all():
            filters = [{"Name": "instance-id", "Values": [instance.id]}]
            eips = ec2client.describe_addresses(Filters=filters)["Addresses"]
            for eip in eips:
                ec2client.disassociate_address(AssociationId=eip["AssociationId"])
                ec2client.release_address(AllocationId=eip["AllocationId"])

    # delete instances
    filters = [
        {"Name": "instance-state-name", "Values": ["running"]},
        {"Name": "vpc-id", "Values": [vpc_id]},
    ]
    ec2_instances = ec2client.describe_instances(Filters=filters)
    instance_ids = []
    for reservation in ec2_instances["Reservations"]:
        instance_ids += [
            instance["InstanceId"] for instance in reservation["Instances"]
        ]

    logger.info(f"instance deletion list: {instance_ids}")
    if instance_ids:
        logging.info("Waiting for instances to terminate")
        waiter = ec2client.get_waiter("instance_terminated")
        responce = ec2client.terminate_instances(InstanceIds=instance_ids)
        logging.info(f"Response: {responce}")
        waiter.wait(InstanceIds=instance_ids)


def destroy_services(vpc_id, aws_region, services):
    services_map = {"ec2": destroy_ec2}

    for service in services.split(","):
        try:
            services_map[service](vpc_id, aws_region)
        except KeyError:
            logger.error(f"destroying {service} not implemented")


def vpc_exists(ec2client, vpc_id):
    try:
        ec2client.describe_vpcs(VpcIds=[vpc_id])
    except ClientError as e:
        logging.info(e)
        return False
    return True


def delete_vpc(vpc_id, aws_region, release_eips=False):
    ec2 = boto3.resource("ec2", region_name=aws_region)
    ec2client = ec2.meta.client
    if not vpc_exists(ec2client, vpc_id):
        print(f"VPC {vpc_id} does not exist in {aws_region}")
        return False

    # Exit cleanly if user did to specify at command line to delete EC2 instances for
    # a VPC with runnining instances
    filters = [
        {"Name": "instance-state-name", "Values": ["running"]},
        {"Name": "vpc-id", "Values": [vpc_id]},
    ]
    if ec2client.describe_instances(Filters=filters)["Reservations"]:
        print(
            f"Running EC2 instances exist in {vpc_id}. Please use --services ec2 to invoke the program."
        )
        return False

    vpc = ec2.Vpc(vpc_id)

    # delete transit gateway attachment for this vpc
    # note - this only handles vpc attachments, not vpn
    for attachment in ec2client.describe_transit_gateway_attachments()[
        "TransitGatewayAttachments"
    ]:
        if attachment["ResourceId"] == vpc_id:
            ec2client.delete_transit_gateway_vpc_attachment(
                TransitGatewayAttachmentId=attachment["TransitGatewayAttachmentId"]
            )

    # delete NAT Gateways
    # attached ENIs are automatically deleted
    # EIPs are disassociated but not released
    filters = [{"Name": "vpc-id", "Values": [vpc_id]}]
    for nat_gateway in ec2client.describe_nat_gateways(Filters=filters)["NatGateways"]:
        ec2client.delete_nat_gateway(NatGatewayId=nat_gateway["NatGatewayId"])

    # detach default dhcp_options if associated with the vpc
    dhcp_options_default = ec2.DhcpOptions("default")
    if dhcp_options_default:
        dhcp_options_default.associate_with_vpc(VpcId=vpc.id)

    # delete any vpc peering connections
    for vpc_peer in ec2client.describe_vpc_peering_connections()[
        "VpcPeeringConnections"
    ]:
        if vpc_peer["AccepterVpcInfo"]["VpcId"] == vpc_id:
            ec2.VpcPeeringConnection(vpc_peer["VpcPeeringConnectionId"]).delete()
        if vpc_peer["RequesterVpcInfo"]["VpcId"] == vpc_id:
            ec2.VpcPeeringConnection(vpc_peer["VpcPeeringConnectionId"]).delete()

    # delete our endpoints
    for ep in ec2client.describe_vpc_endpoints(
        Filters=[{"Name": "vpc-id", "Values": [vpc_id]}]
    )["VpcEndpoints"]:
        ec2client.delete_vpc_endpoints(VpcEndpointIds=[ep["VpcEndpointId"]])

    # delete custom NACLs
    for netacl in vpc.network_acls.all():
        if not netacl.is_default:
            netacl.delete()

    # ensure ENIs are deleted before proceding
    timeout = time.time() + 300
    filter = [{"Name": "vpc-id", "Values": [vpc_id]}]
    logger.info(f"proceed with deleting ENIs")
    reached_timeout = True
    while time.time() < timeout:
        if not ec2client.describe_network_interfaces(Filters=filters)[
            "NetworkInterfaces"
        ]:
            logger.info(f"no ENIs remaining")
            reached_timeout = False
            break
        else:
            logger.info(f"waiting on ENIs to delete")
            client = boto3.client('ec2')
            ec2resouce = boto3.resource('ec2')
            for ni in ec2client.describe_network_interfaces(Filters=filters)["NetworkInterfaces"]:
                print(ni["NetworkInterfaceId"])
                network_interface = ec2resouce.NetworkInterface(ni["NetworkInterfaceId"])
                if "AttachmentId" in ni:
                    network_interface.detach(Force=True)
                time.sleep(10)
                network_interface.delete()
            time.sleep(30)

    if reached_timeout:
        logger.info(f"ENI deletion timed out")
    # delete subnets
    for subnet in vpc.subnets.all():
        for interface in subnet.network_interfaces.all():
            interface.delete()
        subnet.delete()

    # Delete routes, associations, and routing tables
    filter = [{"Name": "vpc-id", "Values": [vpc_id]}]
    route_tables = ec2client.describe_route_tables(Filters=filter)["RouteTables"]
    for route_table in route_tables:
        for route in route_table["Routes"]:
            if route["Origin"] == "CreateRoute":
                ec2client.delete_route(
                    RouteTableId=route_table["RouteTableId"],
                    DestinationCidrBlock=route["DestinationCidrBlock"],
                )
            for association in route_table["Associations"]:
                if not association["Main"]:
                    ec2client.disassociate_route_table(
                        AssociationId=association["RouteTableAssociationId"]
                    )
                    ec2client.delete_route_table(
                        RouteTableId=route_table["RouteTableId"]
                    )
    # delete routing tables without associations
    for route_table in route_tables:
        if route_table["Associations"] == []:
            ec2client.delete_route_table(RouteTableId=route_table["RouteTableId"])

    # destroy NAT gateways
    filters = [{"Name": "vpc-id", "Values": [vpc_id]}]
    nat_gateway_ids = [
        nat_gateway["NatGatewayId"]
        for nat_gateway in ec2client.describe_nat_gateways(Filters=filters)[
            "NatGateways"
        ]
    ]
    for nat_gateway_id in nat_gateway_ids:
        ec2client.delete_nat_gateway(NatGatewayId=nat_gateway_id)

    # detach and delete all IGWs associated with the vpc
    for gw in vpc.internet_gateways.all():
        vpc.detach_internet_gateway(InternetGatewayId=gw.id)
        gw.delete()

    # delete custom security groups
    for sg in vpc.security_groups.all():
        if sg.group_name != "default":
            sg.delete()

    ec2client.delete_vpc(VpcId=vpc_id)
    return True


if __name__ == "__main__":

    parser = argparse.ArgumentParser()
    parser._action_groups.pop()
    required = parser.add_argument_group("required arguments")
    optional = parser.add_argument_group("optional arguments")
    required.add_argument("--vpc_id", required=True, help="Please include your vpc_id")

    optional.add_argument(
        "--services", help="comma-separated list of AWS services to tear down"
    )
    optional.add_argument("--region", help="AWS region")

    args = parser.parse_args()

    if args.region:
        aws_region = args.region
    else:
        aws_region = os.environ["AWS_DEFAULT_REGION"]
    vpc_id = args.vpc_id
    print(f"type: {type(vpc_id)}")
    if args.services:
        logger.info(f"calling destroy_services with {args.services}")
        destroy_services(args.vpc_id, aws_region, args.services)

    logger.info(f"calling delete_vpc with {vpc_id}")
    if delete_vpc(vpc_id=vpc_id, aws_region=aws_region, release_eips=False):
        print(f"destroyed {vpc_id} in {aws_region}")
    else:
        print(f"unable to destroy {vpc_id} in {aws_region}")